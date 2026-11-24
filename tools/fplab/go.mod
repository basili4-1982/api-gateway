module code.sarnas.ru/xgate-tools/fplab

go 1.26.9

require code.sarnas.ru/api-gateway v0.0.0

require code.sarnas.ru/xgate-modules/fingerprint v0.0.0

require github.com/libdns/libdns v1.1.1 // indirect

replace code.sarnas.ru/api-gateway => ../..

replace code.sarnas.ru/xgate-modules/fingerprint => ../../modules/fingerprint
