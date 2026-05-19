import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SearchInput } from "@/components/shared/search-input";
import type { SkillsFilter, SkillsPageState, SkillsSort } from "./lib/skills-page-state";

interface SkillsFilterBarProps {
  state: SkillsPageState;
  onChange: (updates: Partial<SkillsPageState>) => void;
}

export function SkillsFilterBar({ state, onChange }: SkillsFilterBarProps) {
  const { t } = useTranslation("skills");
  const hasFilters = !!state.q || state.filter !== "all" || state.sort !== "name" || !!state.agent;
  const filterOptions: SkillsFilter[] = ["all", "attention", "missing-deps", "disabled", "archived", "unmanaged"];
  const sortOptions: SkillsSort[] = ["name", "deps", "version"];

  return (
    <div className="flex flex-col gap-2 md:flex-row md:items-center">
      <SearchInput
        value={state.q}
        onChange={(q) => onChange({ q })}
        placeholder={t("searchPlaceholder")}
        className="w-full md:max-w-sm"
      />
      <div className="flex flex-wrap items-center gap-2">
        <Select value={state.filter} onValueChange={(filter) => onChange({ filter: filter as SkillsFilter })}>
          <SelectTrigger size="sm" className="w-44">
            <SelectValue aria-label={t("filters.label")} />
          </SelectTrigger>
          <SelectContent>
            {filterOptions.map((filter) => (
              <SelectItem key={filter} value={filter}>{t(`filters.${filter}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={state.sort} onValueChange={(sort) => onChange({ sort: sort as SkillsSort })}>
          <SelectTrigger size="sm" className="w-40">
            <SelectValue aria-label={t("sort.label")} />
          </SelectTrigger>
          <SelectContent>
            {sortOptions.map((sort) => (
              <SelectItem key={sort} value={sort}>{t(`sort.${sort}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="gap-1"
          disabled={!hasFilters}
          onClick={() => onChange({ q: "", filter: "all", sort: "name", agent: null })}
        >
          <X className="h-3.5 w-3.5" />
          {t("filters.clear")}
        </Button>
      </div>
    </div>
  );
}
