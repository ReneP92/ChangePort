import type { Diagnosis } from "@/api/client";
import { useDiagnoses } from "@/api/queries";

/** The diagnoses about each topic and consumer group, for marking them. */
export function useDiagnosesBySubject(): {
  topics: Map<string, Diagnosis[]>;
  groups: Map<string, Diagnosis[]>;
} {
  const diagnoses = useDiagnoses();
  const topics = new Map<string, Diagnosis[]>();
  const groups = new Map<string, Diagnosis[]>();
  for (const diagnosis of diagnoses.data?.diagnoses ?? []) {
    if (diagnosis.consumerGroup !== undefined) {
      groups.set(diagnosis.consumerGroup, [
        ...(groups.get(diagnosis.consumerGroup) ?? []),
        diagnosis,
      ]);
    } else if (diagnosis.topic !== undefined) {
      topics.set(diagnosis.topic, [...(topics.get(diagnosis.topic) ?? []), diagnosis]);
    }
  }
  return { topics, groups };
}
