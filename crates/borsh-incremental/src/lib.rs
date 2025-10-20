use proc_macro::TokenStream;
use quote::{format_ident, quote};
use syn::{parse_macro_input, Attribute, Data, DeriveInput, Expr, Fields, Path};

#[proc_macro_derive(BorshDeserializeIncremental, attributes(incremental))]
pub fn derive(input: TokenStream) -> TokenStream {
    // Parse input token stream into a syntactic representation of the struct
    let input = parse_macro_input!(input as DeriveInput);
    let ident = input.ident;
    let (impl_g, ty_g, where_c) = input.generics.split_for_impl();

    // Parse the struct-level #[incremental(error = ...)] attribute
    let mut error_ty: Option<Path> = None;
    for a in &input.attrs {
        if a.path().is_ident("incremental") {
            let _ = a.parse_nested_meta(|meta| {
                if meta.path.is_ident("error") {
                    let value: Expr = meta.value()?.parse()?;
                    if let Expr::Path(p) = value {
                        error_ty = Some(p.path);
                    } else {
                        return Err(meta.error("expected path for error type"));
                    }
                }
                Ok(())
            });
        }
    }
    // Default to std::io::Error if not provided
    let error_ty = error_ty.unwrap_or_else(|| syn::parse_quote!(::std::io::Error));

    // Extract named fields (only support structs with named fields)
    let fields = match input.data {
        Data::Struct(s) => match s.fields {
            Fields::Named(n) => n.named,
            _ => panic!("BorshDeserializeIncremental only supports named structs"),
        },
        _ => panic!("BorshDeserializeIncremental only supports structs"),
    };

    // Parse per-field #[incremental(...)] attributes: deser_with, default
    fn parse_field_attrs(attrs: &[Attribute]) -> (Option<Path>, Option<Expr>) {
        let mut deser_with: Option<Path> = None;
        let mut default_expr: Option<Expr> = None;
        for a in attrs {
            if a.path().is_ident("incremental") {
                let _ = a.parse_nested_meta(|meta| {
                    if meta.path.is_ident("deser_with") {
                        let value: Expr = meta.value()?.parse()?;
                        if let Expr::Path(p) = value {
                            deser_with = Some(p.path);
                        }
                    } else if meta.path.is_ident("default") {
                        let value: Expr = meta.value()?.parse()?;
                        default_expr = Some(value);
                    }
                    Ok(())
                });
            }
        }
        (deser_with, default_expr)
    }

    // Collect parsed field info
    struct F {
        name: syn::Ident,
        ty: syn::Type,
        deser_with: Option<Path>,
        default_expr: Option<Expr>,
    }
    let mut fdefs = Vec::new();
    for f in fields {
        let name = f.ident.unwrap();
        let ty = f.ty;
        let (deser_with, default_expr) = parse_field_attrs(&f.attrs);
        fdefs.push(F {
            name,
            ty,
            deser_with,
            default_expr,
        });
    }

    // Create unique temporary variable names for each field
    let temps: Vec<_> = fdefs
        .iter()
        .map(|f| format_ident!("__tmp_{}", f.name))
        .collect();

    // Generate deserialization logic for each field
    // - Use deser_with if provided
    // - Otherwise fall back to normal BorshDeserialize
    // - On error or EOF, substitute the configured default or Default::default()
    let reads = fdefs.iter().zip(&temps).map(|(f, tmp)| {
        let ty = &f.ty;
        let fallback = f
            .default_expr
            .as_ref()
            .map(|e| quote!(#e))
            .unwrap_or_else(|| quote!(<#ty as ::core::default::Default>::default()));

        if let Some(dw) = &f.deser_with {
            quote! {
                let before_len = data.len();
                let #tmp: #ty = match (#dw)(&mut data) {
                    Ok(v) => v,
                    Err(e_any) => {
                        let after_len = data.len();
                        if after_len == before_len {
                            #fallback
                        } else {
                            let e: ::std::io::Error = ::core::convert::Into::into(e_any);
                            return Err(<#error_ty as ::core::convert::From<::std::io::Error>>::from(e));
                        }
                    }
                };
            }
        } else {
            quote! {
                let before_len = data.len();
                let #tmp: #ty = match <#ty as ::borsh::BorshDeserialize>::deserialize(&mut data) {
                    Ok(v) => v,
                    Err(e) => {
                        let after_len = data.len();
                        if after_len == before_len {
                            #fallback
                        } else {
                            return Err(<#error_ty as ::core::convert::From<::std::io::Error>>::from(e));
                        }
                    }
                };
            }
        }
    });

    // Assign all temporary variables back into struct fields
    let assigns = fdefs.iter().zip(&temps).map(|(f, tmp)| {
        let name = &f.name;
        quote!(#name: #tmp)
    });

    // Generate TryFrom<&[u8]> impl using the incremental decoding logic
    let expanded = quote! {
        impl #impl_g ::core::convert::TryFrom<&[u8]> for #ident #ty_g #where_c {
            type Error = #error_ty;
            fn try_from(mut data: &[u8]) -> ::core::result::Result<Self, Self::Error> {
                #( #reads )*
                Ok(Self { #( #assigns ),* })
            }
        }
    };

    TokenStream::from(expanded)
}
