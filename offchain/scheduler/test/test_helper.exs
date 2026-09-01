ExUnit.start()

# Define the mock module for NIF boundary testing
Mox.defmock(Scheduler.MockNIF, for: Scheduler.DoubleZeroNIF)
