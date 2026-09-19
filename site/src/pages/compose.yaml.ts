import compose from '../../../compose.yaml?raw';

export const prerender = true;
export function GET() {
  return new Response(compose, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
}
