module github.com/tomlinford/pdelta

go 1.17

// TODO: remove after making sroto public
replace github.com/tomlinford/sroto => ../sroto

require (
	github.com/lyft/protoc-gen-star v0.6.0
	github.com/tomlinford/sroto v0.2.0
	google.golang.org/protobuf v1.27.1
	sigs.k8s.io/yaml v1.3.0
)

require (
	github.com/alvaroloes/enumer v1.1.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-jsonnet v0.18.0 // indirect
	github.com/pascaldekloe/name v1.0.1 // indirect
	github.com/spf13/afero v1.8.0 // indirect
	golang.org/x/mod v0.5.1 // indirect
	golang.org/x/sys v0.0.0-20220111092808-5a964db01320 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/tools v0.1.8 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
