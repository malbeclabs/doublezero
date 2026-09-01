defmodule Scheduler.ValidatorDebt.DebtCollection do
  @moduledoc false
  defstruct total_paid: 0,
            already_paid: 0,
            total_debt: 0,
            total_validators: 0,
            insufficient_funds_count: 0
end

defmodule Scheduler.ValidatorDebt.Debt do
  @moduledoc false
  defstruct [:validator_id, :amount, :result, :success]
end
