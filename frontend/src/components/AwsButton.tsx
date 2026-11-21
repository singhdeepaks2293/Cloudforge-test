import type { ButtonHTMLAttributes, ReactNode } from 'react'

type Variant = 'primary' | 'secondary' | 'link' | 'danger'

const VARIANT_CLS: Record<Variant, string> = {
  // Orange primary button, dark text on hover like the AWS console.
  primary: 'bg-aws-orange text-aws-navy hover:bg-aws-orange-dark hover:text-white border border-[#d97706]',
  secondary: 'bg-white text-aws-text border border-aws-border-dark hover:bg-[#f2f3f3]',
  link: 'bg-transparent text-aws-link border border-transparent hover:underline px-1',
  danger: 'bg-white text-aws-error border border-aws-border-dark hover:bg-aws-error-bg',
}

/** AWS-console-style button. `primary` is the orange action button. */
export function AwsButton({
  variant = 'secondary',
  className = '',
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; children: ReactNode }) {
  return (
    <button
      type="button"
      className={`inline-flex items-center justify-center gap-1.5 rounded-sm px-3 py-1.5 text-sm font-medium disabled:opacity-50 ${VARIANT_CLS[variant]} ${className}`}
      {...rest}
    >
      {children}
    </button>
  )
}
