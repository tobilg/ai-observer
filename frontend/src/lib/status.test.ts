import { describe, it, expect } from 'vitest'
import {
  formatUptime,
  grpcOffReason,
  isPricingStale,
  modeLabel,
  pricingAgeDays,
  pricingUpdatedAt,
  serverUrl,
  shortCommit,
} from './status'

const NOW = Date.parse('2026-11-11T12:00:00Z')
const pricing = (lastUpdated: string, overrideLastUpdated?: string) => ({
  file: 'claude.json', provider: 'anthropic', lastUpdated, models: 10, overrideLastUpdated,
})

describe('status helpers', () => {
  it('ages the prices by the newer of the built-in file and its override', () => {
    expect(pricingAgeDays(pricing('2026-11-05T00:00:00Z'), NOW)).toBe(6)
    expect(isPricingStale(pricing('2026-11-05T00:00:00Z'), NOW)).toBe(false)
    expect(isPricingStale(pricing('2026-11-03T00:00:00Z'), NOW)).toBe(true)
    // A fresh override replaces old built-in prices
    expect(isPricingStale(pricing('2026-01-01T00:00:00Z', '2026-11-10T00:00:00Z'), NOW)).toBe(false)
    expect(pricingUpdatedAt(pricing('2026-01-01T00:00:00Z', '2026-11-10T00:00:00Z'))?.toISOString()).toBe('2026-11-10T00:00:00.000Z')
    expect(pricingAgeDays(pricing('not a date'), NOW)).toBeNull()
    expect(isPricingStale(pricing(''), NOW)).toBe(false)
  })

  it('formats the uptime', () => {
    expect(formatUptime('2026-11-11T11:59:59Z', NOW)).toBe('1 second')
    expect(formatUptime('2026-11-11T11:15:00Z', NOW)).toBe('45 minutes')
    expect(formatUptime('2026-11-11T08:55:00Z', NOW)).toBe('3h 5m')
    expect(formatUptime('2026-11-09T08:00:00Z', NOW)).toBe('2d 4h')
    expect(formatUptime('garbage', NOW)).toBe('—')
  })

  it('names the address clients reach a port at', () => {
    expect(serverUrl({ host: '127.0.0.1' }, 4318, 'localhost')).toBe('http://127.0.0.1:4318')
    expect(serverUrl({ host: '0.0.0.0' }, 4318, 'observer.local')).toBe('http://observer.local:4318')
    expect(serverUrl({ host: '::1' }, 8080, 'localhost')).toBe('http://[::1]:8080')
  })

  it('shortens commits', () => {
    expect(shortCommit('0123456789abcdef0123456789abcdef01234567')).toBe('0123456789ab')
    expect(shortCommit('unknown')).toBe('unknown')
  })

  it('names the server modes', () => {
    expect(modeLabel('serve')).toBe('serve (OTLP)')
    expect(modeLabel('watch')).toBe('watch (session files)')
    expect(modeLabel('serve+watch')).toBe('serve + watch (OTLP and session files)')
  })

  it('tells why OTLP/gRPC is off', () => {
    expect(grpcOffReason({ otlpEnabled: false, otlpGrpcPort: 4317 })).toBe('Off (watch mode)')
    expect(grpcOffReason({ otlpEnabled: true, otlpGrpcPort: 0 })).toBe('Off (AI_OBSERVER_OTLP_GRPC_PORT=off)')
  })
})
