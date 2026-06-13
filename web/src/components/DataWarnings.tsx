interface DataWarningsProps {
  warnings?: string[]
}

export function DataWarnings({ warnings }: DataWarningsProps) {
  if (!warnings?.length) return null

  return (
    <details className="mt-3 text-xs text-zinc-600">
      <summary>警告 {warnings.length}</summary>
      <ul className="mt-1 max-h-40 list-inside list-disc overflow-auto pr-1">
        {warnings.map((warning, index) => (
          <li key={`${warning}-${index}`} className="break-all">
            {warning}
          </li>
        ))}
      </ul>
    </details>
  )
}
