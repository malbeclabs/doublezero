defmodule Scheduler.DoubleZero do
  use Rustler, otp_app: :scheduler, crate: "scheduler_doublezero"

  def pay_debt(_dz_epoch, _ledger_rpc, _solana_rpc), do: :erlang.nif_error(:nif_not_loaded)
  def initialize_distribution(_ledger_rpc, _solana_rpc), do: :erlang.nif_error(:nif_not_loaded)
end
