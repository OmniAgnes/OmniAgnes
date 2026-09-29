import * as React from "react"

import { cn } from "@/lib/utils"

function Badge({ className, variant = "default", ...props }: React.ComponentProps<"span"> & {
  variant?: "default" | "secondary" | "destructive" | "outline"
}) {
  return (
    <span
      data-slot="badge"
      className={cn(
        "inline-flex items-center justify-center rounded-md border px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 gap-1 [&>svg]:size-3",
        {
          default: "border-transparent bg-primary text-primary-foreground",
          secondary: "border-transparent bg-secondary text-secondary-foreground",
          destructive: "border-transparent bg-destructive text-white",
          outline: "text-foreground",
        }[variant],
        className,
      )}
      {...props}
    />
  )
}

export { Badge }
