import * as React from 'react'
import { X } from 'lucide-react'

interface ConfirmDialogProps {
    isOpen: boolean
    onClose: () => void
    onConfirm: () => void
    title: string
    description: string
    confirmText?: string
    cancelText?: string
    variant?: 'default' | 'destructive'
    requiresTyping?: boolean
    typingConfirmation?: string
}

export function ConfirmDialog({
    isOpen,
    onClose,
    onConfirm,
    title,
    description,
    confirmText = 'Confirm',
    cancelText = 'Cancel',
    variant = 'default',
    requiresTyping = false,
    typingConfirmation = 'DELETE',
}: ConfirmDialogProps) {
    const [inputValue, setInputValue] = React.useState('')
    const [isConfirming, setIsConfirming] = React.useState(false)

    const canConfirm = requiresTyping ? inputValue === typingConfirmation : true

    const handleConfirm = () => {
        if (!canConfirm) return
        setIsConfirming(true)
        onConfirm()
    }

    const handleClose = () => {
        if (isConfirming) return
        setInputValue('')
        onClose()
    }

    React.useEffect(() => {
        if (!isOpen) {
            setInputValue('')
            setIsConfirming(false)
        }
    }, [isOpen])

    if (!isOpen) return null

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
            {/* Backdrop */}
            <div
                className="absolute inset-0 bg-black/50 backdrop-blur-sm"
                onClick={handleClose}
            />

            {/* Modal */}
            <div className="relative bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full mx-4 p-6 space-y-4">
                {/* Close button */}
                <button
                    onClick={handleClose}
                    className="absolute top-4 right-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                    disabled={isConfirming}
                >
                    <X className="h-5 w-5" />
                </button>

                {/* Title */}
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white pr-8">
                    {title}
                </h2>

                {/* Description */}
                <div className="text-sm text-gray-600 dark:text-gray-300 whitespace-pre-line">
                    {description}
                </div>

                {/* Typing confirmation input */}
                {requiresTyping && (
                    <div className="space-y-2">
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                            Type <span className="font-mono font-bold">{typingConfirmation}</span> to confirm:
                        </label>
                        <input
                            type="text"
                            value={inputValue}
                            onChange={(e) => setInputValue(e.target.value)}
                            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                            placeholder={typingConfirmation}
                            autoFocus
                            disabled={isConfirming}
                        />
                    </div>
                )}

                {/* Actions */}
                <div className="flex gap-3 justify-end pt-2">
                    <button
                        onClick={handleClose}
                        disabled={isConfirming}
                        className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {cancelText}
                    </button>
                    <button
                        onClick={handleConfirm}
                        disabled={!canConfirm || isConfirming}
                        className={`px-4 py-2 text-sm font-medium text-white rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed ${variant === 'destructive'
                                ? 'bg-red-600 hover:bg-red-700 focus:ring-red-500'
                                : 'bg-blue-600 hover:bg-blue-700 focus:ring-blue-500'
                            }`}
                    >
                        {isConfirming ? 'Processing...' : confirmText}
                    </button>
                </div>
            </div>
        </div>
    )
}
