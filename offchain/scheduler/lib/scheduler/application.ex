defmodule Scheduler.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Scheduler.PromEx,
      {Plug.Cowboy, scheme: :http, plug: Scheduler.Router, options: [port: plug_router_port()]},
      Scheduler.Scheduler
    ]

    opts = [strategy: :one_for_one, name: Scheduler.Supervisor]
    Scheduler.DoubleZero.initialize_tracing_subscriber()
    Supervisor.start_link(children, opts)
  end

  def plug_router_port do
    Application.get_env(:scheduler, :plug_router_port)
  end
end
