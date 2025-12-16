defmodule Scheduler.DoubleZero do
  @moduledoc false
  use Rustler,
    otp_app: :scheduler,
    crate: "scheduler_doublezero",
    mode: if(Mix.env() == :prod, do: :release, else: :debug)

  def initialize_tracing_subscriber, do: :erlang.nif_error(:nif_not_loaded)

  def pay_debt(_dz_epoch, _ledger_rpc, _solana_rpc), do: :erlang.nif_error(:nif_not_loaded)
  def initialize_distribution(_solana_rpc), do: :erlang.nif_error(:nif_not_loaded)

  def calculate_distribution(_dz_epoch, _ledger_rpc, _solana_rpc, _post_to_slack),
    do: :erlang.nif_error(:nif_not_loaded)

  def finalize_distribution(_dz_epoch, _ledger_rpc, _solana_rpc),
    do: :erlang.nif_error(:nif_not_loaded)

  def current_dz_epoch(_ledger_rpc), do: :erlang.nif_error(:nif_not_loaded)

  def post_debt_summary(_insufficient_funds_count, _total_debt, _total_paid),
    do: :erlang.nif_error(:nif_not_loaded)
end
