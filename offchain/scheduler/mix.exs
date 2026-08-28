defmodule Scheduler.MixProject do
  use Mix.Project

  def project do
    [
      app: :scheduler,
      version: "0.1.0",
      elixir: "~> 1.18",
      start_permanent: Mix.env() == :prod,
      test_coverage: [
        threshold: 60,
        ignore_modules: [Scheduler.PromEx, Scheduler.Router, Scheduler.DoubleZero]
      ],
      deps: deps()
    ]
  end

  # Run "mix help compile.app" to learn about applications.
  def application do
    [
      extra_applications: [:logger],
      mod: {Scheduler.Application, []}
    ]
  end

  # Run "mix help deps" to learn about dependencies.
  defp deps do
    [
      {:credo, "~> 1.7"},
      {:mox, "~> 1.0", only: :test},
      {:plug_cowboy, "~> 2.0"},
      {:prom_ex, "~> 1.11"},
      {:quantum, "~> 3.5"},
      {:rustler, "~> 0.37.1"}
    ]
  end
end
