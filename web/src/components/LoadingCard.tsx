interface LoadingCardProps {
  title: string
  testId: string
}

export function LoadingCard({ title, testId }: LoadingCardProps) {
  return (
    <div className="h-40 rounded-lg border border-zinc-800 bg-zinc-900/60 p-4" data-testid={testId}>
      <h3 className="text-sm font-medium text-zinc-300">{title}</h3>
      <div className="mt-4 h-20 animate-pulse rounded bg-zinc-800/70" />
    </div>
  )
}
