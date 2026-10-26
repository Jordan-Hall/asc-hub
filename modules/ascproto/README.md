# ascproto

Go stubs for the asc wire protocol (`asc.v1`), generated from the `proto/` directory of the
`asc` repository. The generated code is committed, so building Forgejo needs neither
`buf` nor `protoc`. Never edit `ascv1/*.pb.go` by hand.

Regenerate from the Forgejo root, with `asc` checked out next to it:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
buf generate                       # reads ../asc/proto (see buf.gen.yaml)
buf generate path/to/proto         # another checkout
```

Pinned tools: `buf` 1.75.0, `protoc-gen-go` v1.36.11 (matches `google.golang.org/protobuf` in
`go.mod`), `protoc-gen-go-grpc` v1.6.2. The server stubs are generated without
`require_unimplemented_servers`, because Forgejo only uses the client side.

The Go package path is set by buf managed mode in `buf.gen.yaml`: `forgejo.org/modules/ascproto/ascv1`.
Use `forgejo.org/modules/ascclient` rather than the raw stubs: it adds authentication, retries,
op ids and verified object fetches.
