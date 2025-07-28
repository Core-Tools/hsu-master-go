module github.com/core-tools/hsu-master-go

go 1.20

replace github.com/core-tools/hsu-core => github.com/core-tools/hsu-core/go v0.0.0-20250728122010-b5337236279f

replace github.com/core-tools/hsu-master => ./

require (
	github.com/core-tools/hsu-core v0.0.0-20250728122010-b5337236279f
	github.com/core-tools/hsu-master v0.0.0-00010101000000-000000000000
	github.com/jessevdk/go-flags v1.6.1
	github.com/stretchr/testify v1.10.0
	go.uber.org/zap v1.20.0
	google.golang.org/grpc v1.50.0
	google.golang.org/protobuf v1.27.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/phayes/freeport v0.0.0-20220201140144-74d24b5ae9f5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.3.3 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
)
