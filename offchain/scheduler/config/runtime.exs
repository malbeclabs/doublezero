import Config

config :scheduler,
  genesis_epoch: 31,
  ledger_rpc: System.get_env("DZ_LEDGER_RPC"),
  solana_rpc: System.get_env("SOLANA_RPC")
