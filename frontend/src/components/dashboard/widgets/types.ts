// What a widget needs of a query (a UseQueryResult fits): its data, whether
// it is loading for the first time or failed, and how to try again
export interface WidgetDataState<T> {
  data: T | undefined
  isPending: boolean
  isError: boolean
  error: unknown
  isFetching: boolean
  refetch: () => unknown
}
