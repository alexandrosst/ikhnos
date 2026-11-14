/** Audit actions read as sentences: "agent-approved" becomes "agent approved". */
export const words = (action: string) => action.replace(/[-_.]/g, ' ')
