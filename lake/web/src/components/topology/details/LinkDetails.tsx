import type { LinkInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { LinkInfoContent, topologyLinkToInfo } from '@/components/shared/LinkInfoContent'

interface LinkDetailsProps {
  link: LinkInfo
}

export function LinkDetails({ link }: LinkDetailsProps) {
  return (
    <div className="p-4">
      <LinkInfoContent link={topologyLinkToInfo(link)} compact />
    </div>
  )
}

// Header content for the panel
export function LinkDetailsHeader({ link }: LinkDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        link
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink to={`/dz/links/${link.pk}`}>
          {link.code}
        </EntityLink>
      </div>
      <div className="text-xs text-muted-foreground mt-0.5">
        <EntityLink to={`/dz/devices/${link.deviceAPk}`}>{link.deviceACode}</EntityLink>
        {' ↔ '}
        <EntityLink to={`/dz/devices/${link.deviceZPk}`}>{link.deviceZCode}</EntityLink>
      </div>
    </>
  )
}
