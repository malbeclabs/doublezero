import Config

config :scheduler, Scheduler.Scheduler,
  jobs: [
    {"0 */2 * * *", {Scheduler.Worker.CollectAllDebt, :start_link, []}},
    {"*/2 * * * *", {Scheduler.Worker.InitializeDistribution, :start_link, []}},
    {"30 */2 * * *", {Scheduler.Worker.CalculateDistribution, :start_link, []}}
  ]
