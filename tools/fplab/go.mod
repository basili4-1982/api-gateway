module code.sarnas.ru/xgate-tools/fplab

go 1.26.9

require code.sarnas.ru/api-gateway v0.0.0

require (
	code.sarnas.ru/xgate-modules/fingerprint v0.0.0
	github.com/quic-go/quic-go v0.64.1
)

require (
	github.com/libdns/libdns v1.1.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace code.sarnas.ru/api-gateway => ../..

replace code.sarnas.ru/xgate-modules/fingerprint => ../../modules/fingerprint
