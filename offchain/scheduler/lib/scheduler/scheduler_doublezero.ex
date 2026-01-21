defmodule Scheduler.DoubleZero do
  @moduledoc false
  use Rustler,
    otp_app: :scheduler,
    crate: "scheduler_doublezero",
    mode: if(Mix.env() == :prod, do: :release, else: :debug)

  def initialize_tracing_subscriber, do: :erlang.nif_error(:nif_not_loaded)

  def collect_all_debt(_solana_rpc), do: :erlang.nif_error(:nif_not_loaded)

  def collect_epoch_debt(_dz_epoch, _solana_rpc),
    do: :erlang.nif_error(:nif_not_loaded)

  def initialize_distribution(_solana_rpc), do: :erlang.nif_error(:nif_not_loaded)

  def calculate_distribution(_solana_rpc, _post_to_slack),
    do: :erlang.nif_error(:nif_not_loaded)

  def finalize_distribution(_dz_epoch, _solana_rpc),
    do: :erlang.nif_error(:nif_not_loaded)
end
