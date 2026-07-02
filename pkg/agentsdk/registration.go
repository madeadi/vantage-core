package agentsdk

type RegisterResponse struct {
	Token    string `json:"token"`
	GRPCAddr string `json:"grpc_addr"`
}
