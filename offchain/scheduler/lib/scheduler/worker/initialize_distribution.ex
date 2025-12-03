defmodule Scheduler.Worker.InitializeDistribution do
  @moduledoc """
    - GenServer that initializes a distribution
    - it runs once a minute and either initializes the distribution or shuts down
  """
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  def init([] = state) do
    {:ok, state, {:continue, :initialize_distribution}}
  end

  def handle_continue(:initialize_distribution, state) do
    case Scheduler.DoubleZero.initialize_distribution(ledger_rpc(), solana_rpc()) do
      {:error, error} ->
        Logger.error("initialize_distribution: received error: #{inspect(error)}")

      {:ok, msg} ->
        Logger.info("initialize_distribution: completed with msg: #{msg}")
    end

    {:stop, :shutdown, state}
  end

  def handle_info(msg, state) do
    Logger.warning("Received unexpected msg: #{msg}")
    {:noreply, state}
  end

  defp ledger_rpc do
    Application.get_env(:scheduler, :ledger_rpc)
  end

  defp solana_rpc do
    Application.get_env(:scheduler, :solana_rpc)
  end
end
