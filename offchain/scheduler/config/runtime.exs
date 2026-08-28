import Config

config :scheduler,
  genesis_epoch: 31,
  ledger_rpc: System.get_env("DZ_LEDGER_RPC"),
  solana_rpc: System.get_env("SOLANA_RPC"),
  prometheus_endpoint: System.get_env("PROMETHEUS_ENDPOINT", "localhost"),
  plug_router_port: String.to_integer(System.get_env("SCHEDULER_HTTP_PORT", "4001"))

config :scheduler, Scheduler.PromEx,
  disabled: false,
  manual_metrics_start_delay: :no_delay,
  drop_metrics_groups: [],
  grafana: :disabled,
  metrics_server: :disabled
