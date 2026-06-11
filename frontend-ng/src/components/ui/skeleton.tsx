import { cn } from "@/lib/utils"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-[var(--r-sm)] bg-[var(--surface-3)]", className)}
      {...props}
    />
  )
}

export { Skeleton }
