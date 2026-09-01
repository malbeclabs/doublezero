import Config

config :scheduler,
  genesis_epoch: 31,
  prometheus_endpoint: System.get_env("PROMETHEUS_ENDPOINT", "localhost"),
  plug_router_port: String.to_integer(System.get_env("SCHEDULER_HTTP_PORT", "4001"))

# Every worker hands this value straight to the NIF, which wants a usable URL. An
# unset variable used to arrive there as nil, so fail the boot instead. fetch_env!
# alone is not enough: it raises only when the variable is missing, so SOLANA_RPC=
# would pass an empty string through to the same place. Tests set the key
# themselves and never read this.
if config_env() != :test do
  solana_rpc = System.fetch_env!("SOLANA_RPC")

  if String.trim(solana_rpc) == "" do
    raise "SOLANA_RPC is set but empty. The workers pass it straight to the NIF."
  end

  config :scheduler, solana_rpc: solana_rpc
end

config :scheduler, Scheduler.PromEx,
  disabled: false,
  manual_metrics_start_delay: :no_delay,
  drop_metrics_groups: [],
  grafana: :disabled,
  metrics_server: :disabled
