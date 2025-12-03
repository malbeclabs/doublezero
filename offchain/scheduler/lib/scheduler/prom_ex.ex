defmodule Scheduler.PromEx do
  @moduledoc false

  use PromEx, otp_app: :scheduler

  alias PromEx.Plugins

  @impl true
  def plugins do
    [
      Plugins.Application,
      Plugins.Beam
    ]
  end

  @impl true
  def dashboard_assigns do
    [
      datasource_id: prometheus_endpoint(),
      default_selected_interval: "30s"
    ]
  end

  @impl true
  def dashboards do
    [
      {:prom_ex, "application.json"},
      {:prom_ex, "beam.json"}
    ]
  end

  def prometheus_endpoint do
    Application.get_env(:scheduler, :prometheus_endpoint)
  end
end
