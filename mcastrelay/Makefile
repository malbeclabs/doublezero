.PHONY: proto clean

PROTO_DIR := proto/relay
GEN_DIR := proto/relay/gen/pb-go

proto:
	@mkdir -p $(GEN_DIR)
	protoc --proto_path=$(PROTO_DIR) \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		relay.proto

clean:
	rm -rf $(GEN_DIR)
