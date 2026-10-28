export function Greeting({ name }: { name?: string }) @{
  const message = name ? `Hello, ${name}` : 'Hello, stranger';

  <>
    <div class="card">
      <p>{message}</p>
    </div>

    <style>
      .card { padding: 1rem; }
    </style>
  </>
}
