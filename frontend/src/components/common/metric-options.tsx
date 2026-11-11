import { NativeSelectOptGroup, NativeSelectOption } from '@/components/ui/native-select'
import { getMetricMetadata, getSourceDisplayName } from '@/lib/metricMetadata'
import { METRIC_GROUP_ORDER, type MetricGroups } from '@/lib/metricGroups'

// The options of a metric NativeSelect, in one option group per tool. Metrics of
// unknown tools keep their full name: display names drop the first segment.
export function MetricOptions({ groups }: { groups: MetricGroups }) {
  return (
    <>
      {METRIC_GROUP_ORDER.filter((group) => groups[group].length > 0).map((group) => (
        <NativeSelectOptGroup key={group} label={group === 'other' ? 'Other' : getSourceDisplayName(group)}>
          {groups[group].map((name) => (
            <NativeSelectOption key={name} value={name}>
              {group === 'other' ? name : getMetricMetadata(name).displayName}
            </NativeSelectOption>
          ))}
        </NativeSelectOptGroup>
      ))}
    </>
  )
}
