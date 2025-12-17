.PHONY: proto
proto:
	docker pull namely/protoc-all
	docker run -v $(PWD)/proto/flow:/defs namely/protoc-all --go-source-relative -f flow.proto -l go
