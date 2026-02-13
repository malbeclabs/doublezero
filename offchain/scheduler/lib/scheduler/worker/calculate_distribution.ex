defmodule Scheduler.Worker.CalculateDistribution do
  @moduledoc """
    Calculates distribution for the current dz epoch - 1 (most recently closed epoch) and runs three times to ensure that the outcome is the same (this is done in worker.rs calculate_distribution) for calculated debt. If it's equal for three runs, the debt is finalized.
  """
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    state = %{count: 0}
    GenServer.start_link(__MODULE__, state, name: __MODULE__)
  end

  def init(state) do
    {:ok, state, {:continue, :calculate_distribution}}
  end

  def handle_continue(:calculate_distribution, %{count: 2} = state) do
    case nif_module().calculate_distribution(
           solana_rpc(),
           true
         ) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")
        {:stop, :shutdown, state}

      {:ok, dz_epoch} ->
        Logger.info("Proceeding to finalize debt for dz epoch #{dz_epoch}")
        state = Map.put(state, :dz_epoch, dz_epoch)
        {:noreply, state, {:continue, :finalize_distribution}}
    end
  end

  def handle_continue(:calculate_distribution, state) do
    case nif_module().calculate_distribution(
           solana_rpc(),
           false
         ) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")
        {:stop, :shutdown, state}

      {:ok, _dz_epoch} ->
        state = %{state | count: state.count + 1}
        Logger.info("Completed calculation for debt ##{state.count}")
        {:noreply, state, {:continue, :calculate_distribution}}
    end
  end

  def handle_continue(:finalize_distribution, state) do
    Logger.info("Finalizing debt for dz epoch #{state.dz_epoch}")

    case nif_module().finalize_distribution(state.dz_epoch, solana_rpc()) do
      {:error, error} ->
        Logger.error("calculate_distribution: received error: #{inspect(error)}")

      _ ->
        Logger.info(
          "calculate_distribution: finalized distribution for dz epoch #{state.dz_epoch}"
        )
    end

    {:stop, :normal, state}
  end

  def handle_info(msg, state) do
    Logger.warning("Received unexpected msg: #{msg}")
    {:noreply, state}
  end

  defp solana_rpc do
    Application.get_env(:scheduler, :solana_rpc)
  end

  defp nif_module do
    Application.get_env(:scheduler, :nif_module, Scheduler.DoubleZero)
  end
end
