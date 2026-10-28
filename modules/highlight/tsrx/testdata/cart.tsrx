import type { Item } from './types';

function Cart({ items }: { items: Item[] }) @{
  const subtotal = items.reduce((sum, item) => sum + item.price, 0);
  const discount = subtotal > 100 ? 0.1 : 0;
  const pairs = new Map<string, number>();

  <div>
    <h2>Your cart</h2>
    <p>Subtotal: ${subtotal}</p>
    <p>Save: ${(subtotal * discount).toFixed(2)}</p>
    <img src="/cart.png" alt="" />
  </div>
}

export const total = 1_000 < 2000;
