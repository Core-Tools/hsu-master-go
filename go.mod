module github.com/core-tools/hsu-master-go

go 1.22.3

replace github.com/core-tools/hsu-core => github.com/core-tools/hsu-core/go v0.0.0-20250720170041-9937b0bb0529

replace github.com/core-tools/hsu-master => .

require (
	github.com/core-tools/hsu-core v0.0.0-20250720170041-9937b0bb0529
	github.com/core-tools/hsu-master v0.0.0-00010101000000-000000000000
	github.com/jessevdk/go-flags v1.6.1
	github.com/stretchr/testify v1.10.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.36.6
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/phayes/freeport v0.0.0-20220201140144-74d24b5ae9f5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
)
