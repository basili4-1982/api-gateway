package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dockerClient — минимальный клиент Docker/Podman Engine API. Работает как
// через unix socket (unix://...), так и по HTTP(S) (для тестов и удалённого API).
type dockerClient struct {
	baseURL    string
	apiVersion string
	http       *http.Client
}

func newDockerClient(host, apiVersion string) (*dockerClient, error) {
	c := &dockerClient{apiVersion: strings.Trim(apiVersion, "/"), http: &http.Client{Timeout: 10 * time.Second}}

	switch {
	case strings.HasPrefix(host, "unix://"):
		socket := strings.TrimPrefix(host, "unix://")
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		}
		c.http.Transport = transport
		c.baseURL = "http://docker"
	case strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		c.baseURL = strings.TrimRight(host, "/")
	default:
		return nil, fmt.Errorf("unsupported discovery host: %q", host)
	}
	return c, nil
}

func (c *dockerClient) url(path string) string {
	if c.apiVersion == "" {
		return c.baseURL + path
	}
	return c.baseURL + "/" + c.apiVersion + path
}

func (c *dockerClient) listContainers(ctx context.Context, labelPrefix string) ([]Container, error) {
	filters := fmt.Sprintf(`{"label":["%s.enable=true"]}`, labelPrefix)
	u := c.url("/containers/json") + "?all=0&filters=" + url.QueryEscape(filters)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker list failed: %s", resp.Status)
	}

	var raw []struct {
		ID              string            `json:"Id"`
		Names           []string          `json:"Names"`
		Labels          map[string]string `json:"Labels"`
		Ports           []Port            `json:"Ports"`
		NetworkSettings struct {
			Networks map[string]Network `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, Container{
			ID:       r.ID,
			Names:    r.Names,
			Labels:   r.Labels,
			Ports:    r.Ports,
			Networks: r.NetworkSettings.Networks,
		})
	}
	return out, nil
}

// events отдаёт канал с именами действий контейнерных событий. Канал закрывается
// при завершении потока или отмене ctx. Неизвестные действия тоже передаются —
// вызывающая сторона трактует их как повод для ре-синка.
func (c *dockerClient) events(ctx context.Context) (<-chan string, error) {
	u := c.url("/events") + "?filters=" + url.QueryEscape(`{"type":["container"]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("docker events failed: %s", resp.Status)
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for {
			var ev struct {
				Type   string `json:"Type"`
				Action string `json:"Action"`
			}
			if err := dec.Decode(&ev); err != nil {
				return
			}
			if ev.Type != "container" || ev.Action == "" {
				continue
			}
			select {
			case ch <- ev.Action:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
