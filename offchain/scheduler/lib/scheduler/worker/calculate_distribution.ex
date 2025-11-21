defmodule Scheduler.Worker.CalculateDistribution do
  @moduledoc """
    Calculates distribution for the current dz epoch - 1 (most recently closed epoch) and runs three times to ensure that the outcome is the same (this is done in worker.rs calculate_distribution) for calculated debt. If it's equal for three runs, the debt is finalized.
  """
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    state = %{count: 0, dz_epoch: nil}
    GenServer.start_link(__MODULE__, state, name: __MODULE__)
  end

  def init(state) do
    {:ok, state, {:continue, :get_current_dz_epoch}}
  end

  def handle_continue(:get_current_dz_epoch, state) do
    case Scheduler.DoubleZero.current_dz_epoch(ledger_rpc()) do
      dz_epoch when is_integer(dz_epoch) ->
        # dz_epoch - 1 gets the most recently closed dz_epoch
        epoch_to_calculate = dz_epoch - 1

        Logger.info("Calculating debt for dz epoch #{epoch_to_calculate}")

        state = %{state | dz_epoch: epoch_to_calculate}
        {:noreply, state, {:continue, :calculate_distribution}}

      error ->
        Logger.error("calculate_distribution: failed to get dz_epoch: #{inspect(error)}")
        {:stop, :shutdown, state}
    end
  end

  def handle_continue(:calculate_distribution, %{count: 2} = state) do
    case Scheduler.DoubleZero.calculate_distribution(
           state.dz_epoch,
           ledger_rpc(),
           solana_rpc(),
           true
         ) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")
        {:stop, :shutdown, state}

      _ ->
        Logger.info("Proceeding to finalize debt")
        {:noreply, state, {:continue, :finalize_distribution}}
    end
  end

  def handle_continue(:calculate_distribution, state) do
    case Scheduler.DoubleZero.calculate_distribution(
           state.dz_epoch,
           ledger_rpc(),
           solana_rpc(),
           false
         ) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")
        {:stop, :shutdown, state}

      _ ->
        state = %{state | count: state.count + 1}
        Logger.info("Completed calculation for debt ##{state.count}")
        {:noreply, state, {:continue, :calculate_distribution}}
    end
  end

  def handle_continue(:finalize_distribution, state) do
    Logger.info("Finalizing debt for dz epoch #{state.dz_epoch}")

    case Scheduler.DoubleZero.finalize_distribution(state.dz_epoch, ledger_rpc(), solana_rpc()) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")

      _ ->
        Logger.info(
          "calculate_distribution: finalized distribution for dz epoch #{state.dz_epoch}"
        )
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
