import type { DeviceInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { DeviceInfoContent, type DeviceInfoData } from '@/components/shared/DeviceInfoContent'

interface DeviceDetailsProps {
  device: DeviceInfo
}

/**
 * Convert topology DeviceInfo to shared DeviceInfoData
 */
function toDeviceInfoData(device: DeviceInfo): DeviceInfoData {
  return {
    pk: device.pk,
    code: device.code,
    deviceType: device.deviceType,
    status: device.status,
    metroPk: device.metroPk,
    metroName: device.metroName,
    contributorPk: device.contributorPk,
    contributorCode: device.contributorCode,
    userCount: device.userCount,
    validatorCount: device.validatorCount,
    stakeSol: device.stakeSol,
    stakeShare: device.stakeShare,
    interfaces: device.interfaces || [],
  }
}

export function DeviceDetails({ device }: DeviceDetailsProps) {
  return (
    <div className="p-4">
      <DeviceInfoContent device={toDeviceInfoData(device)} compact />
    </div>
  )
}

// Header content for the panel
export function DeviceDetailsHeader({ device }: DeviceDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        device
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink to={`/dz/devices/${device.pk}`}>
          {device.code}
        </EntityLink>
      </div>
    </>
  )
}
