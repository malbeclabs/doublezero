import { useParams, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Cable, AlertCircle, ArrowLeft } from 'lucide-react'
import { fetchLink } from '@/lib/api'
import { LinkInfoContent, linkDetailToInfo } from '@/components/shared/LinkInfoContent'

export function LinkDetailPage() {
  const { pk } = useParams<{ pk: string }>()
  const navigate = useNavigate()

  const { data: link, isLoading, error } = useQuery({
    queryKey: ['link', pk],
    queryFn: () => fetchLink(pk!),
    enabled: !!pk,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !link) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Link not found</div>
          <button
            onClick={() => navigate('/dz/links')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to links
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-[1200px] mx-auto px-4 sm:px-8 py-8">
        {/* Back button */}
        <button
          onClick={() => navigate('/dz/links')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to links
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Cable className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{link.code}</h1>
            <div className="text-sm text-muted-foreground">{link.link_type}</div>
          </div>
        </div>

        {/* Link info content */}
        <LinkInfoContent link={linkDetailToInfo(link)} />
      </div>
    </div>
  )
}
