# Scheduler

Scheduler is an Elixir application designed to automate and manage the lifecycle of debts within a financial system. It schedules, tracks, and processes various stages of debt management, such as creation, payment reminders, overdue notifications, and closure. The scheduler ensures that all debt-related events are handled in a timely and reliable manner, reducing manual intervention and improving operational efficiency.

## How It Works

The scheduler operates by periodically checking the status of debts and triggering appropriate actions based on predefined rules and schedules. For example, it can send reminders before payment due dates, escalate overdue debts, and mark debts as resolved once payments are completed. The system is designed to be extensible, allowing for the addition of new lifecycle events as business requirements evolve.

## Running the Application

To run the scheduler locally:

1. Ensure you have Elixir installed. You can download it from [elixir-lang.org](https://elixir-lang.org/install.html).
2. Clone this repository and navigate to the project directory.
3. Install dependencies:

   ```sh
   mix deps.get
## Installation

If [available in Hex](https://hex.pm/docs/publish), the package can be installed
by adding `scheduler` to your list of dependencies in `mix.exs`:

```elixir
def deps do
  [
    {:scheduler, "~> 0.1.0"}
  ]
end
```

To add additional supervised processes, there are two required changes:

The first is updating `config.exs` with the cron-like syntax of how often the process will be run and then the Module, Function, Arity (MFA) format. Since the workers are almost certainly GenServers, they will follow this format -
`{"some interval", {WorkerModuleName, :start_link, []}}`. The Module is `WorkerModuleName`, the function is `start_link` and the arity is an empty list `[]`.

The second is creating a worker in the `worker` subdirectory. Using the GenServer behaviour (interface), it's trivial to fill out the details:

```elixir
defmodule Scheduler.Worker.PayDebt do
  use GenServer

  require Logger

  def start_link(_var \\ []) do
    # state = %{} whatever startup state
    GenServer.start_link(__MODULE__, state, name: __MODULE__) # this calls the `init/1` callback
  end

  def init(state) do
    # most likely you will want to have the GenServer automatically continue the loop with {:continue, _} as this example shows
    {:ok, state, {:continue, :your_callback_name}}
  end

  def handle_info(:info_name, state) do
  # logic here
  # here you can either do nothing with {:noreply, state} or continue the loop with `{:noreply, state, {:continue, :your_callback_name}}
  {:noreply, state}
  end

  ## this is a catch-all for unexpected messages and is standard practice
  def handle_info(msg, state) do
    Logger.warning("Received unexpected msg: #{msg}")
    {:noreply, state}
  end

  ## handle_continue/2 callbacks are called automatically triggered by the {:continue, _} tuple
  def handle_continue(:your_callback_name, state) do
    # logic goes here
    # can call this in a loop or at some interval with Process.send_after/4 - `handle_info/2 receives the message from send_after
    {:noreply, state}
  end
end
```