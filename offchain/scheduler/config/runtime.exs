import Config

config :scheduler,
  genesis_epoch: 31,
  prometheus_endpoint: System.get_env("PROMETHEUS_ENDPOINT", "localhost"),
  plug_router_port: String.to_integer(System.get_env("SCHEDULER_HTTP_PORT", "4001"))

# Every worker hands this value straight to the NIF, which wants a String. An
# unset variable used to arrive there as nil, so fail the boot instead. Tests
# set the key themselves and never read this.
if config_env() != :test do
  config :scheduler, solana_rpc: System.fetch_env!("SOLANA_RPC")
end

config :scheduler, Scheduler.PromEx,
  disabled: false,
  manual_metrics_start_delay: :no_delay,
  drop_metrics_groups: [],
  grafana: :disabled,
  metrics_server: :disabled
