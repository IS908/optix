interface DataWarningsProps {
  warnings?: string[]
}

export function DataWarnings({ warnings }: DataWarningsProps) {
  if (!warnings?.length) return null

  return (
    <details className="mt-3 text-xs text-zinc-600">
      <summary>警告 {warnings.length}</summary>
      <ul className="mt-1 list-inside list-disc">
        {warnings.map((warning, index) => (
          <li key={`${warning}-${index}`}>{warning}</li>
        ))}
      </ul>
    </details>
  )
}
