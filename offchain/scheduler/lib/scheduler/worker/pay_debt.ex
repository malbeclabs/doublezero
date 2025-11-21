defmodule Scheduler.Worker.PayDebt do
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    state = %{genesis_epoch: genesis_epoch(), current_epoch: genesis_epoch()}
    GenServer.start_link(__MODULE__, state, name: __MODULE__)
  end

  def init(state) do
    {:ok, state, {:continue, :queue_debt_payment}}
  end

  def handle_info(:pay_debt, state) do
    case Scheduler.DoubleZero.pay_debt(state.current_epoch, ledger_rpc(), solana_rpc()) do
      ## Retry if we get this error because solana timed out
      {:error,
       "Unhandled Solana RPC error: unable to confirm transaction. This can happen in situations such as transaction expiration and insufficient fee-payer funds"} ->
        {:noreply, state, {:continue, :queue_debt_payment}}

      {:error, error} ->
        ## one of these errors is reached when we have exceeded a finalized distribution
        if String.contains?(error, "Record account not found at address") ||
             String.contains?(error, "Failed to fetch record") do
          Logger.info("scheduler completed sweep at epoch #{state.current_epoch}")
          {:stop, :shutdown, state}
        else
          Logger.error(
            "scheduler encountered unexpected error at epoch #{state.current_epoch}: #{inspect(error)}"
          )

          {:stop,:shutdown, state}
        end

      _ ->
        Logger.info("completed epoch #{state.current_epoch}")
        state = %{state | current_epoch: state.current_epoch + 1}
        {:noreply, state, {:continue, :queue_debt_payment}}
    end
  end

  def handle_info(msg, state) do
    Logger.warning("Received unexpected msg: #{msg}")
    {:noreply, state}
  end

  def handle_continue(:queue_debt_payment, state) do
    Process.send_after(self(), :pay_debt, 10)
    {:noreply, state}
  end

  defp genesis_epoch do
    Application.get_env(:scheduler, :genesis_epoch)
  end

  defp ledger_rpc do
    Application.get_env(:scheduler, :ledger_rpc)
  end

  defp solana_rpc do
    Application.get_env(:scheduler, :solana_rpc)
  end
end
