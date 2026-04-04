/**
 * Reusable form controls for the AI defaults section:
 * - SubSection: collapsible group panel with title and description
 * - Field: labeled input with optional InfoLabel tooltip
 */
import { ChevronDown, ChevronRight } from "lucide-react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { InfoLabel } from "@/components/shared/info-label";

/* eslint-disable @typescript-eslint/no-explicit-any */

export function SubSection({
  title,
  desc,
  open,
  onToggle,
  children,
}: {
  title: string;
  desc: string;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-md border">
      <button
        type="button"
        className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-muted/50"
        onClick={onToggle}
      >
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        <div>
          <span className="font-medium">{title}</span>
          <span className="ml-2 text-xs text-muted-foreground">{desc}</span>
        </div>
      </button>
      {open && <div className="space-y-3 border-t px-4 py-3">{children}</div>}
    </div>
  );
}

export function Field({
  label,
  tip,
  value,
  onChange,
  placeholder,
  type = "text",
  step,
}: {
  label: string;
  tip?: string;
  value: any;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  step?: string;
}) {
  return (
    <div className="grid gap-1.5">
      {tip ? <InfoLabel tip={tip}>{label}</InfoLabel> : <Label>{label}</Label>}
      <Input
        type={type}
        step={step}
        value={value ?? ""}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
      />
    </div>
  );
}
