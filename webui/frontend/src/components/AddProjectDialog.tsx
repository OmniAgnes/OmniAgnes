import { useEffect, useState } from "react"
import { ArrowUpIcon, FolderIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { api } from "@/lib/api"
import type { BrowseResult } from "@/types"

interface AddProjectDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAdded: () => void
}

export function AddProjectDialog({ open, onOpenChange, onAdded }: AddProjectDialogProps) {
  const [pathInput, setPathInput] = useState("")
  const [browse, setBrowse] = useState<BrowseResult | null>(null)
  const [err, setErr] = useState("")

  useEffect(() => {
    if (!open) return
    setErr("")
    api.browse("").then((r) => {
      setBrowse(r)
      setPathInput(r.path)
    }).catch((e) => setErr(String(e)))
  }, [open])

  const goto = (p: string) => {
    api.browse(p).then(setBrowse).catch((e) => setErr(String(e)))
  }

  const add = async (p: string) => {
    setErr("")
    try {
      await api.addProject(p)
      onAdded()
      onOpenChange(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>添加项目</DialogTitle>
          <DialogDescription>选择一个本地文件夹，或直接粘贴路径。</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <div className="flex gap-2">
            <Input
              value={pathInput}
              onChange={(e) => setPathInput(e.target.value)}
              placeholder="C:\path\to\project"
              onKeyDown={(e) => {
                if (e.key === "Enter") void add(pathInput)
              }}
            />
            <Button variant="secondary" onClick={() => void add(pathInput)}>
              添加
            </Button>
          </div>

          {browse && (
            <div className="flex flex-col gap-1.5">
              <div className="text-muted-foreground flex items-center justify-between text-xs">
                <span className="flex items-center gap-1">
                  <FolderIcon className="size-3.5" /> {browse.path}
                </span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-1.5 text-xs"
                  disabled={!browse.parent}
                  onClick={() => goto(browse.parent)}
                >
                  <ArrowUpIcon className="size-3.5" /> 上一级
                </Button>
              </div>
              <ScrollArea className="h-48 rounded-md border">
                <div className="flex flex-col p-1">
                  {browse.entries.map((e) => (
                    <button
                      key={e.path}
                      type="button"
                      className="hover:bg-accent flex items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm cursor-pointer"
                      onDoubleClick={() => void add(e.path)}
                      onClick={() => goto(e.path)}
                      title={`${e.path}（双击直接添加）`}
                    >
                      <FolderIcon className="text-muted-foreground size-4 shrink-0" />
                      <span className="truncate">{e.name}</span>
                    </button>
                  ))}
                  {browse.entries.length === 0 && (
                    <div className="text-muted-foreground px-2 py-3 text-xs">没有子文件夹</div>
                  )}
                </div>
              </ScrollArea>
              <Button variant="secondary" size="sm" onClick={() => void add(browse.path)}>
                把当前目录（{browse.path}）添加为项目
              </Button>
            </div>
          )}

          {err && <div className="text-destructive text-sm">{err}</div>}
        </div>

        <DialogFooter />
      </DialogContent>
    </Dialog>
  )
}
