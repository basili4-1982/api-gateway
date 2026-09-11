package config

// Merge возвращает новый конфиг: base + обнаруженные таргеты и правила.
// Base не мутируется. При конфликте приоритет у статики: таргет с уже
// существующим именем и правило с уже существующей парой (host, path_prefix)
// пропускаются. Обнаруженные правила, ссылающиеся на неизвестный таргет,
// отбрасываются.
//
// Merge копирует Config поверхностно: вложенные срезы и указатели (Targets,
// Routing.Rules, TLS и т.п.) переиспользуются. Мутировать их на месте нельзя —
// заменяйте на новые срезы, как это делается ниже.
func Merge(base *Config, targets []TargetConfig, rules []RoutingRule) *Config {
	merged := *base

	merged.Targets = make([]TargetConfig, 0, len(base.Targets)+len(targets))
	merged.Targets = append(merged.Targets, base.Targets...)

	staticNames := make(map[string]bool, len(base.Targets))
	for _, t := range base.Targets {
		staticNames[t.Name] = true
	}
	discoveredNames := make(map[string]bool, len(targets))
	for _, t := range targets {
		if staticNames[t.Name] || discoveredNames[t.Name] {
			continue
		}
		discoveredNames[t.Name] = true
		merged.Targets = append(merged.Targets, t)
	}

	merged.Routing.Rules = make([]RoutingRule, 0, len(base.Routing.Rules)+len(rules))
	merged.Routing.Rules = append(merged.Routing.Rules, base.Routing.Rules...)

	seenRules := make(map[string]bool, len(base.Routing.Rules))
	for _, r := range base.Routing.Rules {
		seenRules[ruleKey(r.Host, r.PathPrefix)] = true
	}
	for _, r := range rules {
		key := ruleKey(r.Host, r.PathPrefix)
		if seenRules[key] {
			continue
		}
		// Правило принимаем только если его таргет реально добавлен из discovery:
		// при коллизии имени со статикой обнаруженный таргет отброшен, значит и
		// его правила не должны маршрутизироваться на статику.
		if !discoveredNames[r.TargetName] {
			continue
		}
		seenRules[key] = true
		merged.Routing.Rules = append(merged.Routing.Rules, r)
	}

	return &merged
}

func ruleKey(host, pathPrefix string) string {
	return host + "\x00" + pathPrefix
}
