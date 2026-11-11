import { useState, useCallback } from 'react'

/**
 * Hook for persisting state to localStorage with type safety.
 * Uses lazy initialization to read from localStorage on first render.
 *
 * @param key - The localStorage key
 * @param defaultValue - Default value if key doesn't exist
 * @returns [storedValue, setValue] - Current value and setter function
 */
export function useLocalStorage<T>(
  key: string,
  defaultValue: T
): [T, (value: T | ((prev: T) => T)) => void] {
  // Lazy initialization: read from localStorage on first render only
  const [storedValue, setStoredValue] = useState<T>(() => {
    try {
      const item = localStorage.getItem(key)
      if (item !== null) {
        return JSON.parse(item)
      }
    } catch (error) {
      console.warn(`Error reading localStorage key "${key}":`, error)
    }
    return defaultValue
  })

  // Setter that also persists to localStorage
  const setValue = useCallback((value: T | ((prev: T) => T)) => {
    setStoredValue(prev => {
      const valueToStore = value instanceof Function ? value(prev) : value
      try {
        localStorage.setItem(key, JSON.stringify(valueToStore))
      } catch (error) {
        console.warn(`Error writing localStorage key "${key}":`, error)
      }
      return valueToStore
    })
  }, [key])

  return [storedValue, setValue]
}
