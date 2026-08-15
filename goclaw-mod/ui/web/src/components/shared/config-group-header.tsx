interface ConfigGroupHeaderProps {
  title: string;
  description: string;
}

/** Section group header for organizing config sections into logical groups. */
export function ConfigGroupHeader({ title, description }: ConfigGroupHeaderProps) {
  return (
    <div className="space-y-0.5 pt-4 first:pt-0">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h4>
      <p className="text-xs text-muted-foreground">{description}</p>
    </div>
  );
}
