defmodule Scheduler.WorkerIntegrationTest do
  @moduledoc """
  Integration tests for scheduler workers using Mox to mock the NIF boundary.

  These tests verify the GenServer behavior with mocked NIF responses,
  allowing us to test:
  - Happy path: NIF returns success
  - Error path: NIF returns error, GenServer handles gracefully
  - CalculateDistribution 3-run confirmation logic
  """
  use ExUnit.Case, async: false

  import ExUnit.CaptureLog
  import Mox

  # Use global mode for cross-process mocking (GenServers spawn in separate processes)
  setup :set_mox_global
  setup :verify_on_exit!

  # Set the mock module for tests
  setup do
    Application.put_env(:scheduler, :nif_module, Scheduler.MockNIF)
    Application.put_env(:scheduler, :solana_rpc, "http://localhost:8899")

    on_exit(fn ->
      Application.delete_env(:scheduler, :nif_module)
       Application.delete_env(:scheduler, :solana_rpc)
    end)

    :ok
  end

  describe "InitializeDistribution worker" do
    test "stops normally on successful NIF call" do
      expect(Scheduler.MockNIF, :initialize_distribution, fn _rpc ->
        {}
      end)

      log =
        capture_log(fn ->
          # Trap exits so we can monitor without crashing
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.InitializeDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "completed"
    end

    test "stops with shutdown on NIF error" do
      expect(Scheduler.MockNIF, :initialize_distribution, fn _rpc ->
        {:error, "RPC connection failed"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.InitializeDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :shutdown
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "error"
      assert log =~ "RPC connection failed"
    end
  end

  describe "CollectAllDebt worker" do
    test "stops normally on successful NIF call" do
      expect(Scheduler.MockNIF, :collect_all_debt, fn _rpc ->
        {}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CollectAllDebt.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "Successfully collected debts"
    end

    test "stops normally even on NIF error (fire and forget)" do
      expect(Scheduler.MockNIF, :collect_all_debt, fn _rpc ->
        {:error, "Some error occurred"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CollectAllDebt.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              # CollectAllDebt stops with :normal even on error
              assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "error"
    end
  end

  describe "CalculateDistribution worker 3-run confirmation logic" do
    test "runs calculate 3 times then finalizes on success" do
      # First two calls with post_to_slack=false
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:ok, 42}
      end)

      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:ok, 42}
      end)

      # Third call with post_to_slack=true (count == 2)
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, true ->
        {:ok, 42}
      end)

      # Then finalize with dz_epoch=42
      expect(Scheduler.MockNIF, :finalize_distribution, fn 42, _rpc ->
        {}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      # Verify the progression
      assert log =~ "Completed calculation for debt #1"
      assert log =~ "Completed calculation for debt #2"
      assert log =~ "Proceeding to finalize debt for dz epoch 42"
      assert log =~ "finalized distribution for dz epoch 42"
    end

    test "passes dz_epoch from calculate to finalize" do
      dz_epoch = 99

      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false -> {:ok, dz_epoch} end)
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false -> {:ok, dz_epoch} end)
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, true -> {:ok, dz_epoch} end)

      expect(Scheduler.MockNIF, :finalize_distribution, fn ^dz_epoch, _rpc ->
        {}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} -> assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "Finalizing debt for dz epoch 99"
      assert log =~ "finalized distribution for dz epoch 99"
    end

    test "stops with shutdown on first calculation error" do
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:error, "Calculation failed"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :shutdown
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "error"
      assert log =~ "Calculation failed"
    end

    test "stops with shutdown on second calculation error" do
      # First call succeeds
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:ok, 42}
      end)

      # Second call fails
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:error, "Second calculation failed"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :shutdown
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "Completed calculation for debt #1"
      assert log =~ "error"
      assert log =~ "Second calculation failed"
    end

    test "stops with shutdown on third calculation error" do
      # First two calls succeed
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:ok, 42}
      end)

      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false ->
        {:ok, 42}
      end)

      # Third call fails
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, true ->
        {:error, "Third calculation failed"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              assert reason == :shutdown
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "Completed calculation for debt #1"
      assert log =~ "Completed calculation for debt #2"
      assert log =~ "error"
      assert log =~ "Third calculation failed"
    end

    test "finalization error logs but worker stops normally" do
      # All three calculations succeed
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false -> {:ok, 42} end)
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, false -> {:ok, 42} end)
      expect(Scheduler.MockNIF, :calculate_distribution, fn _rpc, true -> {:ok, 42} end)

      # Finalization fails
      expect(Scheduler.MockNIF, :finalize_distribution, fn 42, _rpc ->
        {:error, "Finalization failed"}
      end)

      log =
        capture_log(fn ->
          Process.flag(:trap_exit, true)
          {:ok, pid} = Scheduler.Worker.CalculateDistribution.start_link()

          receive do
            {:EXIT, ^pid, reason} ->
              # Worker still stops normally after finalization (fire and forget)
              assert reason == :normal
          after
            5000 -> flunk("Worker did not stop in time")
          end
        end)

      assert log =~ "Proceeding to finalize debt"
      assert log =~ "error"
      assert log =~ "Finalization failed"
    end
  end
end
