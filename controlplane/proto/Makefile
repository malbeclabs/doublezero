.PHONY: proto
proto:
	docker pull namely/protoc-all
	docker run -v $(PWD)/proto/controller/:/defs namely/protoc-all --go-source-relative -f controller.proto -l go