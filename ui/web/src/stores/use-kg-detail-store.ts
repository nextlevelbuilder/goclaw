import { create } from "zustand";
import type { KGEntity, KGRelation, KGTraversalResult } from "@/types/knowledge-graph";

/** Dedicated store for KG entity detail dialog — isolated from main graph data. */
interface KGDetailState {
  entity: KGEntity | null;
  relations: KGRelation[];
  traversalResults: KGTraversalResult[];
  traversing: boolean;
  depth: number;

  setEntity: (entity: KGEntity | null) => void;
  setRelations: (relations: KGRelation[]) => void;
  setTraversalResults: (results: KGTraversalResult[]) => void;
  setTraversing: (v: boolean) => void;
  setDepth: (d: number) => void;
  reset: () => void;
}

const initialState = {
  entity: null as KGEntity | null,
  relations: [] as KGRelation[],
  traversalResults: [] as KGTraversalResult[],
  traversing: false,
  depth: 3,
};

export const useKGDetailStore = create<KGDetailState>((set) => ({
  ...initialState,
  setEntity: (entity) => set({ entity }),
  setRelations: (relations) => set({ relations }),
  setTraversalResults: (traversalResults) => set({ traversalResults }),
  setTraversing: (traversing) => set({ traversing }),
  setDepth: (depth) => set({ depth }),
  reset: () => set({ ...initialState }),
}));
