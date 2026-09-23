.PHONY: dev
dev:
	$(MAKE) -j2 dev-core dev-ui

dev-core:
	air -c .air.core.toml

dev-mqtt-agent:
	air -c .air.mqtt-agent-example.toml

dev-ui:
	cd ui && npm run dev


.PHONY: proto
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       -I . \
	       proto/agent/v1/agent.proto \
	       proto/mission/v1/mission.proto
	protoc --go_out=. --go_opt=paths=source_relative \
	       --connect-go_out=. --connect-go_opt=paths=source_relative \
	       -I . \
	       proto/api/v1/task.proto \
	       proto/api/v1/agent.proto \
	       proto/api/v1/mission.proto \
	       proto/api/v1/telemetry.proto
