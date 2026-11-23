UPDATE orders SET status = 'paid' WHERE total_cents > 0 AND status = 'pending';
