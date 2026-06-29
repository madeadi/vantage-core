dev-core:
	air -c .air.core.toml

dev-smallbot:
	air -c .air.smallbot.toml

dev-sps-mr:
	air -c .air.sps-mr.toml

.PHONY: proto
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       proto/agent/v1/agent.proto \
	       proto/mission/v1/mission.proto
