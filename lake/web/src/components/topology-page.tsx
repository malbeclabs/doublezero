import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '@/lib/api'
import { TopologyMap } from '@/components/topology-map'

export function TopologyPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    refetchInterval: 60000, // Refresh every minute
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-muted-foreground">Loading topology...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-destructive">
          Failed to load topology: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-muted-foreground">No topology data available</div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-0 h-screen w-screen">
      <TopologyMap
        metros={data.metros}
        devices={data.devices}
        links={data.links}
      />
    </div>
  )
}
