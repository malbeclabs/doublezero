package config

const (
	// Mainnet constants.
	MainnetLedgerPublicRPCURL         = "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab"
	MainnetServiceabilityProgramID    = "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv"
	MainnetTelemetryProgramID         = "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC"
	MainnetInternetLatencyCollectorPK = "8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH"
	MainnetDeviceLocalASN             = 209321
	// MainnetTwoZOracleURL              = "https://10qk1zz9ql.execute-api.us-east-1.amazonaws.com/mainnet-beta/api/v1"
	MainnetTwoZOracleURL     = ""
	MainnetSolanaRPC         = "https://api.mainnet-beta.solana.com"
	MainnetControllerAddress = "controller-grpc.mainnet-beta.doublezero.xyz:443"

	// Testnet constants.
	TestnetLedgerPublicRPCURL         = "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16"
	TestnetServiceabilityProgramID    = "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb"
	TestnetTelemetryProgramID         = "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1"
	TestnetInternetLatencyCollectorPK = "HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1"
	TestnetDeviceLocalASN             = 65342
	TestnetTwoZOracleURL              = "https://7op1fv6sj5.execute-api.us-east-1.amazonaws.com/testnet/api/v1"
	TestnetSolanaRPC                  = "https://api.testnet.solana.com"
	TestnetControllerAddress          = "controller-grpc.testnet.doublezero.xyz:443"

	// Devnet constants.
	DevnetLedgerPublicRPCURL         = "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16"
	DevnetServiceabilityProgramID    = "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah"
	DevnetTelemetryProgramID         = "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"
	DevnetInternetLatencyCollectorPK = "3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ"
	DevnetDeviceLocalASN             = 21682
	DevnetTwoZOracleURL              = ""
	DevnetControllerAddress          = "controller-grpc.devnet.doublezero.xyz:443"
)
