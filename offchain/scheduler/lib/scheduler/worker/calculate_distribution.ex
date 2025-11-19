defmodule Scheduler.Worker.CalculateDistribution do
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  def init([] = state) do
    {:ok, state, {:continue, :calculate_distribution}}
  end

  def handle_continue(:calculate_distribution, state) do
    case Scheduler.DoubleZero.calculate_distribution(ledger_rpc(), solana_rpc()) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")

      _ ->
        :ok
    end

    {:stop, "calculate_distribution shutting down", state}
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
