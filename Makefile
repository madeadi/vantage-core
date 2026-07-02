dev-core:
	air -c .air.core.toml

dev-smallbot:
	air -c .air.smallbot.toml

dev-sps-mr:
	air -c .air.sps-mr.toml

dev-sps-mission:
	air -c .air.sps-mission.toml

.PHONY: dev-sps
dev-sps:
	$(MAKE) -j3 dev-core dev-sps-mr dev-sps-mission

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
	       proto/api/v1/mission.proto
