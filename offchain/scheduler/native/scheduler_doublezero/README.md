# NIF for Scheduler.DoubleZero

## To build the NIF module:

- Your NIF will now build along with your project.

## To load the NIF:

```elixir
defmodule Scheduler.DoubleZero do
  use Rustler, otp_app: :scheduler, crate: "scheduler_doublezero"

  # When your NIF is loaded, it will override this function.
  def pay_debt(_debtor, _amount), do: :erlang.nif_error(:nif_not_loaded)
end
```

## Examples

[This](https://github.com/rusterlium/NifIo) is a complete example of a NIF written in Rust.
