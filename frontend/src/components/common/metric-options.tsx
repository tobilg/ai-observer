import { getMetricMetadata, getSourceDisplayName } from '@/lib/metricMetadata'
import { METRIC_GROUP_ORDER, type MetricGroups } from '@/lib/metricGroups'

// The <option>s of a metric <select>, in one <optgroup> per tool. Metrics of
// unknown tools keep their full name: display names drop the first segment.
export function MetricOptions({ groups }: { groups: MetricGroups }) {
  return (
    <>
      {METRIC_GROUP_ORDER.filter((group) => groups[group].length > 0).map((group) => (
        <optgroup key={group} label={group === 'other' ? 'Other' : getSourceDisplayName(group)}>
          {groups[group].map((name) => (
            <option key={name} value={name}>
              {group === 'other' ? name : getMetricMetadata(name).displayName}
            </option>
          ))}
        </optgroup>
      ))}
    </>
  )
}
