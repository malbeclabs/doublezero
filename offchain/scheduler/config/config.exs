import Config

config :scheduler, Scheduler.Scheduler,
  jobs: [
    {"0 */2 * * *", {Scheduler.Worker.PayDebt, :start_link, []}},
    {"*/2 * * * *", {Scheduler.Worker.InitializeDistribution, :start_link, []}}
  ]
