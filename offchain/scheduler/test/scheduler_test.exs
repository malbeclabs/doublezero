defmodule SchedulerTest do
  use ExUnit.Case
  doctest Scheduler

  test "greets the world" do
    assert Scheduler.hello() == :world
  end
end
