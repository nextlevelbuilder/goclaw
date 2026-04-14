import { Search } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { useDebounce } from "@/hooks/use-debounce";
import { useState, useEffect, useRef } from "react";

interface SearchInputProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  delay?: number;
}

export function SearchInput({
  value,
  onChange,
  placeholder,
  className,
  delay = 300,
}: SearchInputProps) {
  const { t } = useTranslation("common");
  const [local, setLocal] = useState(value);
  const debounced = useDebounce(local, delay);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    onChangeRef.current(debounced);
  }, [debounced]);

  useEffect(() => {
    setLocal(value);
  }, [value]);

  return (
    <div className={`relative ${className ?? ""}`}>
      <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        placeholder={placeholder ?? t("searchPlaceholder")}
        className="pl-9"
      />
    </div>
  );
}
