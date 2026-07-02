package missionsdk

type BaseConfig struct {
	ID       string `yaml:"id"`
	Key      string `yaml:"key"`
	CoreUrl  string `yaml:"core_url"`
	GrpcAddr string `yaml:"grpc_addr"`
}

type MissionConfig struct {
	ID  string `yaml:"id"`
	Key string `yaml:"key"`
}

type CoreServerConfig struct {
	CoreUrl  string `yaml:"core_url"`
	GrpcAddr string `yaml:"grpc_addr"`
}
