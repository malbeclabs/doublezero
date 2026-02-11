defmodule Scheduler.DoubleZeroNIF do
  @moduledoc """
  Behaviour module for DoubleZero NIF functions.

  This module defines callbacks for all NIF functions, allowing for
  dependency injection in tests. The actual implementation is in
  `Scheduler.DoubleZero`, which uses Rustler to load the native code.

  ## Usage in Workers

  Workers should use a private `nif_module/0` function to allow injection:

      defp nif_module do
        Application.get_env(:scheduler, :nif_module, Scheduler.DoubleZero)
      end

      def handle_continue(:do_work, state) do
        case nif_module().some_function(arg) do
          {:ok, result} -> ...
          {:error, error} -> ...
        end
      end

  ## Testing with Mox

  In test_helper.exs:

      Mox.defmock(Scheduler.MockNIF, for: Scheduler.DoubleZeroNIF)

  In tests:

      import Mox

      setup :verify_on_exit!

      test "worker handles success" do
        expect(Scheduler.MockNIF, :some_function, fn _arg ->
          {:ok, "result"}
        end)
        # ... test code
      end
  """

  @doc """
  Initialize the tracing subscriber for logging.
  Returns an empty tuple on success.
  """
  @callback initialize_tracing_subscriber() :: {} | {:error, term()}

  @doc """
  Collect all outstanding debt across all epochs.

  ## Parameters
    - `solana_rpc`: The Solana RPC URL to connect to

  ## Returns
    - `{}` on success
    - `{:error, reason}` on failure
  """
  @callback collect_all_debt(solana_rpc :: String.t()) :: {} | {:error, term()}

  @doc """
  Collect debt for a specific DZ epoch.

  ## Parameters
    - `dz_epoch`: The DoubleZero epoch number
    - `solana_rpc`: The Solana RPC URL to connect to

  ## Returns
    - `{}` on success
    - `{:error, reason}` on failure
  """
  @callback collect_epoch_debt(dz_epoch :: non_neg_integer(), solana_rpc :: String.t()) ::
              {} | {:error, term()}

  @doc """
  Initialize distribution for the current epoch.

  ## Parameters
    - `solana_rpc`: The Solana RPC URL to connect to

  ## Returns
    - `{}` on success
    - `{:error, reason}` on failure
  """
  @callback initialize_distribution(solana_rpc :: String.t()) :: {} | {:error, term()}

  @doc """
  Calculate validator debt distribution.

  ## Parameters
    - `solana_rpc`: The Solana RPC URL to connect to
    - `post_to_slack`: Whether to post results to Slack

  ## Returns
    - `{}` on success (or any non-error tuple)
    - `{:error, reason}` on failure
  """
  @callback calculate_distribution(solana_rpc :: String.t(), post_to_slack :: boolean()) ::
              {:ok, non_neg_integer()} | {:error, term()}

  @doc """
  Finalize the distribution for a specific epoch.

  ## Parameters
    - `dz_epoch`: The DoubleZero epoch number
    - `solana_rpc`: The Solana RPC URL to connect to

  ## Returns
    - `{}` on success
    - `{:error, reason}` on failure
  """
  @callback finalize_distribution(dz_epoch :: non_neg_integer(), solana_rpc :: String.t()) ::
              {} | {:error, term()}
end
