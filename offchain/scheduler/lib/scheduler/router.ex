defmodule Scheduler.Router do
  use Plug.Router
  plug(PromEx.Plug, prom_ex_module: Scheduler.PromEx)
  plug(Plug.Telemetry, event_prefix: [:doublezero_offchain_scheduler])
  plug(Plug.Logger)
  plug(:match)
  plug(:dispatch)

  get "/health_check" do
    send_resp(conn, 200, ":ok")
  end

  match _ do
    send_resp(conn, 404, "not found")
  end
end
