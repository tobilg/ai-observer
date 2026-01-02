import { useState, useEffect, useRef } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Upload, FileJson, AlertCircle, Link, Loader2 } from 'lucide-react'
import type { DashboardExport } from '@/types/dashboard-export'
import { validateDashboardImport, fetchDashboardFromUrl } from '@/lib/dashboard-export'

interface DashboardDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  mode: 'create' | 'edit'
  initialName?: string
  initialDescription?: string
  onSubmit: (name: string, description?: string) => Promise<void>
  onImport?: (data: DashboardExport) => Promise<void>
}

export function DashboardDialog({
  open,
  onOpenChange,
  mode,
  initialName = '',
  initialDescription = '',
  onSubmit,
  onImport,
}: DashboardDialogProps) {
  const [name, setName] = useState(initialName)
  const [description, setDescription] = useState(initialDescription)
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<'create' | 'import'>('create')

  // Import-specific state
  const [importSource, setImportSource] = useState<'file' | 'url'>('file')
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importUrl, setImportUrl] = useState('')
  const [importData, setImportData] = useState<DashboardExport | null>(null)
  const [importErrors, setImportErrors] = useState<string[]>([])
  const [fetchingUrl, setFetchingUrl] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Reset form when dialog opens
  useEffect(() => {
    if (open) {
      setName(initialName)
      setDescription(initialDescription)
      setActiveTab('create')
      setImportSource('file')
      setImportFile(null)
      setImportUrl('')
      setImportData(null)
      setImportErrors([])
      setFetchingUrl(false)
    }
  }, [open, initialName, initialDescription])

  const handleFileSelect = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    setImportFile(file)
    setImportErrors([])
    setImportData(null)

    try {
      const text = await file.text()
      const json = JSON.parse(text)
      const result = validateDashboardImport(json)

      if (result.valid && result.data) {
        setImportData(result.data)
      } else {
        setImportErrors(result.errors)
      }
    } catch {
      setImportErrors(['Invalid JSON file'])
    }
  }

  const handleFetchUrl = async () => {
    if (!importUrl.trim()) return

    setFetchingUrl(true)
    setImportErrors([])
    setImportData(null)

    const result = await fetchDashboardFromUrl(importUrl.trim())

    if (result.error) {
      setImportErrors([result.error])
    } else if (result.data) {
      const validation = validateDashboardImport(result.data)
      if (validation.valid && validation.data) {
        setImportData(validation.data)
      } else {
        setImportErrors(validation.errors)
      }
    }

    setFetchingUrl(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    setLoading(true)
    try {
      if (activeTab === 'import' && importData && onImport) {
        await onImport(importData)
      } else {
        if (!name.trim()) return
        await onSubmit(name.trim(), description.trim() || undefined)
      }
      onOpenChange(false)
    } catch (error) {
      console.error('Failed to save dashboard:', error)
    } finally {
      setLoading(false)
    }
  }

  // For edit mode, don't show tabs
  if (mode === 'edit') {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-[425px]">
          <form onSubmit={handleSubmit}>
            <DialogHeader>
              <DialogTitle>Rename Dashboard</DialogTitle>
              <DialogDescription>Update the dashboard name.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid gap-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="My Dashboard"
                  autoFocus
                />
              </div>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!name.trim() || loading}>
                {loading ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    )
  }

  // Create mode with tabs
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Create Dashboard</DialogTitle>
            <DialogDescription>
              Create a new dashboard or import from a file.
            </DialogDescription>
          </DialogHeader>

          <Tabs
            value={activeTab}
            onValueChange={(v) => setActiveTab(v as 'create' | 'import')}
            className="mt-4"
          >
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="create">Create New</TabsTrigger>
              <TabsTrigger value="import">Import</TabsTrigger>
            </TabsList>

            <TabsContent value="create" className="space-y-4 pt-4">
              <div className="grid gap-2">
                <Label htmlFor="create-name">Name</Label>
                <Input
                  id="create-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="My Dashboard"
                  autoFocus
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="create-description">Description (optional)</Label>
                <Input
                  id="create-description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="Dashboard description..."
                />
              </div>
            </TabsContent>

            <TabsContent value="import" className="space-y-4 pt-4">
              {/* Import source toggle */}
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant={importSource === 'file' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => {
                    setImportSource('file')
                    setImportErrors([])
                    setImportData(null)
                  }}
                >
                  <Upload className="h-4 w-4 mr-1" />
                  From File
                </Button>
                <Button
                  type="button"
                  variant={importSource === 'url' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => {
                    setImportSource('url')
                    setImportErrors([])
                    setImportData(null)
                  }}
                >
                  <Link className="h-4 w-4 mr-1" />
                  From URL
                </Button>
              </div>

              {importSource === 'file' ? (
                <>
                  {/* Hidden file input */}
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept=".json,application/json"
                    onChange={handleFileSelect}
                    className="hidden"
                  />

                  {/* Drop zone / file selector */}
                  <div
                    onClick={() => fileInputRef.current?.click()}
                    className="border-2 border-dashed border-muted-foreground/25 rounded-lg p-8 text-center cursor-pointer hover:border-muted-foreground/50 transition-colors"
                  >
                    {importFile ? (
                      <div className="flex items-center justify-center gap-2">
                        <FileJson className="h-5 w-5 text-muted-foreground" />
                        <span className="text-sm">{importFile.name}</span>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        <Upload className="h-8 w-8 mx-auto text-muted-foreground" />
                        <p className="text-sm text-muted-foreground">
                          Click to select a dashboard JSON file
                        </p>
                      </div>
                    )}
                  </div>
                </>
              ) : (
                <>
                  {/* URL input */}
                  <div className="grid gap-2">
                    <Label htmlFor="import-url">Dashboard URL</Label>
                    <div className="flex gap-2">
                      <Input
                        id="import-url"
                        type="url"
                        value={importUrl}
                        onChange={(e) => setImportUrl(e.target.value)}
                        placeholder="https://example.com/dashboard.json"
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault()
                            handleFetchUrl()
                          }
                        }}
                      />
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={handleFetchUrl}
                        disabled={!importUrl.trim() || fetchingUrl}
                      >
                        {fetchingUrl ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          'Fetch'
                        )}
                      </Button>
                    </div>
                  </div>
                </>
              )}

              {/* Validation errors */}
              {importErrors.length > 0 && (
                <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                  <div className="flex items-start gap-2">
                    <AlertCircle className="h-4 w-4 mt-0.5 shrink-0" />
                    <div>
                      <p className="font-medium">Invalid file:</p>
                      <ul className="mt-1 list-disc list-inside">
                        {importErrors.slice(0, 5).map((error, i) => (
                          <li key={i}>{error}</li>
                        ))}
                        {importErrors.length > 5 && (
                          <li>...and {importErrors.length - 5} more errors</li>
                        )}
                      </ul>
                    </div>
                  </div>
                </div>
              )}

              {/* Preview of valid import */}
              {importData && (
                <div className="rounded-md bg-muted p-3 text-sm">
                  <p className="font-medium">{importData.name}</p>
                  {importData.description && (
                    <p className="text-muted-foreground mt-1">{importData.description}</p>
                  )}
                  <p className="text-muted-foreground mt-2">
                    {importData.widgets.length} widget
                    {importData.widgets.length !== 1 ? 's' : ''}
                  </p>
                </div>
              )}
            </TabsContent>
          </Tabs>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                loading ||
                (activeTab === 'create' && !name.trim()) ||
                (activeTab === 'import' && !importData)
              }
            >
              {loading
                ? 'Creating...'
                : activeTab === 'import'
                  ? 'Import'
                  : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
