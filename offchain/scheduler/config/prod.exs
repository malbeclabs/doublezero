import Config

config :logger, level: :info

config :scheduler,
  plug_router_port: String.to_integer(System.get_env("SCHEDULER_HTTP_PORT", "4001")),
  prometheus_endpoint: System.get_env("PROMETHEUS_ENDPOINT", "localhost")
