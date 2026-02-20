module github.com/organic-programming/grace-op

go 1.25.1

require (
	github.com/organic-programming/go-holons v0.2.1-0.20260212114054-8fbeaa095fb9
	github.com/organic-programming/sophia-who v0.1.0
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
	nhooyr.io/websocket v1.8.17
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
)

replace github.com/organic-programming/go-holons => ../../sdk/go-holons

replace github.com/organic-programming/sophia-who => ../sophia-who
