defmodule Scheduler.Worker.CollectAllDebt do
  @moduledoc """
    - Genserver that collects debt
    - it runs every two hours
    - currently it starts at the genesis epoch of 31
    - once debt forgiveness comes into play, that will change
  """
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    state = %{}

    GenServer.start_link(__MODULE__, state, name: __MODULE__)
  end

  def init(state) do
    {:ok, state, {:continue, :collect_all_debt}}
  end

  def handle_info(msg, state) do
    Logger.warning("Received unexpected msg: #{msg}")
    {:noreply, state}
  end

  def handle_continue(:collect_all_debt, state) do
    # TODO: figure out why the remote env sends out an empty
    # total debt collection
    Process.sleep(100)
    case Scheduler.DoubleZero.collect_all_debt(ledger_rpc(), solana_rpc()) do
      {} ->
        Logger.info("Successfully collected debts for all epochs")

      {:error, error} ->
        Logger.error("CollectAllDebt worker encountered an error: #{inspect(error)}")
    end

    {:stop, :normal, state}
  end

  defp ledger_rpc do
    Application.get_env(:scheduler, :ledger_rpc)
  end

  defp solana_rpc do
    Application.get_env(:scheduler, :solana_rpc)
  end
end
