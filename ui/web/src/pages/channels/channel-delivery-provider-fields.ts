export function isDeliveryProviderKey(key: string) {
  return /^chat_behavior\.(quick_ack|intermediate_replies)\.provider$/.test(key);
}

export function isDeliveryModelKey(key: string) {
  return /^chat_behavior\.(quick_ack|intermediate_replies)\.model$/.test(key);
}

export function deliveryModelKey(providerKey: string) {
  return providerKey.replace(/\.provider$/, ".model");
}
